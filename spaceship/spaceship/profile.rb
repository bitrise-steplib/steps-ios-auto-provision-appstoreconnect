require 'spaceship'

class Cert
    attr_accessor :id
end

def find_profile(profile_name)
    platform = "ios"
    profiles = ProfileClient.fetch_profiles(false, platform)

    for 
    base64_pem = Base64.encode64(downloaded_portal_cert.to_pem)

    cert_info = {
      content: base64_pem,
      id: cert.id
    }
end

def create_profile(bundle_id, certificate_id, profile_name)
    cert = Cert.new
    cert.id = certificate_id
    profile = Spaceship::Portal.provisioning_profile.development.create!(bundle_id: bundle_id, certificate: cert, name: profile_name, sub_platform: nil)
end