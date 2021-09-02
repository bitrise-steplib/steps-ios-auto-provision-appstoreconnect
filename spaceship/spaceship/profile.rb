require 'spaceship'

class Cert
    attr_accessor :id
end

def create_profile(bundle_id, certificate_id, profile_name)
    cert = Cert.new
    cert.id = certificate_id
    profile = Spaceship::Portal.provisioning_profile.development.create!(bundle_id: bundle_id, certificate: cert, name: profile_name, sub_platform: nil)
end