require 'spaceship'
require_relative 'log'

class Cert
  attr_accessor :id
end

class Profile
  attr_accessor :id
end

def list_profiles(profile_type, name)
  profiles = []
  profile_class = portal_profile_class(profile_type)
  run_or_raise_preferred_error_message do
    profiles = profile_class.all(mac: false, xcode: false)
  end
  matching_profiles = profiles.select { |prof| prof.name == name }

  profile_infos = []
  matching_profiles.each do |profile|
    Log.info("#{profile}")
    profile_base64 = Base64.encode64(profile.download)

    profile_info = {
      id: profile.id,
      uuid: profile.uuid,
      name: profile.name,
      # @example Active (profile is fine)
      #   "Active"
      # @example Expired (time ran out)
      #   "Expired"
      # @example Invalid (e.g. code signing identity not available any more)
      #   "Invalid"
      status: profile.status,
      expiry: profile.expires,
      content: profile_base64,
      app_id: profile.app.app_id,
      bundle_id: profile.app.bundle_id,
      certificates: profile.certificates.map(&:id),
      devices: profile.devices.map(&:id)
    }
    profile_infos.append(profile_info)
  end

  profile_infos
end

def delete_profile(id)
  Log.info("DELETE PROFILE #{id}")

  profile = Spaceship::Portal::ProvisioningProfile.new
  profile.id = id
  profile.delete!
end

def create_profile(profile_type, bundle_id, certificate_id, profile_name)
  list_profiles(profile_type, profile_name)

  cert = Cert.new
  cert.id = certificate_id

  profile = nil
  profile_class = portal_profile_class(profile_type)
  run_or_raise_preferred_error_message do
    profile = profile_class.create!(bundle_id: bundle_id, certificate: cert, name: profile_name, sub_platform: nil)
  end

  Log.info("Created profile: #{profile}")
  profile_base64 = Base64.encode64(profile.download)
  {
    id: profile.id,
    uuid: profile.uuid,
    name: profile.name,
    status: profile.status,
    expiry: profile.expires,
    platform: 'IOS',
    content: profile_base64,
    app_id: profile.app.app_id,
    bundle_id: profile.app.bundle_id
  }
end

def portal_profile_class(distribution_type)
  case distribution_type
  when 'IOS_APP_DEVELOPMENT'
    Spaceship::Portal.provisioning_profile.development
  when 'IOS_APP_STORE'
    Spaceship::Portal.provisioning_profile.app_store
  when 'IOS_APP_ADHOC'
    Spaceship::Portal.provisioning_profile.ad_hoc
  when 'IOS_APP_INHOUSE'
    Spaceship::Portal.provisioning_profile.in_house
  else
    raise "invalid distribution type provided: #{distribution_type}, available: [IOS_APP_DEVELOPMENT, IOS_APP_STORE, IOS_APP_ADHOC, IOS_APP_INHOUSE]"
  end
end